"use strict";

if (Gorse === undefined) {
	var Gorse = {};
}

//! write a message to the console
/*!
 * @param string msg
 *
 * @return void
 */
Gorse.log = function(msg) {
	if (!window || !window.console || !window.console.log ||
			msg === undefined) {
		return;
	}
	window.console.log(msg);
};

Gorse.toggle_read = function(item_li) {
	if ($(item_li).hasClass('clicked')) {
		$(item_li).removeClass('clicked');

		// remove the input indicating it is clicked.
		$('input.read_item', item_li).remove();

		Gorse.update_counts();

		return;
	}

	$(item_li).addClass('clicked');

	// add an input so it will be submitted as clicked.
	var read_ele = $('<input>').attr({
		'type': 'hidden',
		'name': 'read_item',
		'class': 'read_item',
		'value': $('.item_id', item_li).val()
	});

	$(item_li).append(read_ele);

	Gorse.update_counts();
};

// Determine how many items we have selected. Update a displayed counter.
Gorse.update_counts = function() {
	// Count how many we have selected.
	var count = $('input.read_item').length;

	// Display the counter in the save button.
	var label = 'Save (' + count + ')';
	$('button#update-flags-top').text(label);
};

$(document).ready(function() {
	// add a click handler to all feed item rows.
	$('ul#items > li').each(function(i, li) {
		// when we click an item row, we change its class to show
		// it is clicked, and set a form input to indicate it is clicked.
		$(li).click(function() {
			Gorse.toggle_read($(this));
		});
	});

	// And to the checkmark
  $('a.check-it').each(function() {
		$(this).click(function() {
			var item_li = $(this).closest('li');
			Gorse.toggle_read(item_li);
			return false;
		});
	});

	// hide the list of feed information by default.
	$('ul#feeds').hide();

	// setup the 'feed expand' link.
	$('a.expand_feeds')
		.text('+ Expand')
		.click(function(evt) {
			evt.preventDefault();

			if ($(this).text() === '+ Expand') {
				$(this).text('- Collapse');
				$('#feeds').show(400);
			} else {
				$(this).text('+ Expand');
				$('#feeds').hide(400);
			}
		});

	// when we click the save button, submit the form with our
	// read elements.
	$('button#update-flags-top').click(function() {
		$('form#list-items-form').submit();
	});
});
